<template>
  <v-row justify="center" align="center">
    <v-dialog v-model="newDialog" max-width="480px">
      <v-form>
        <v-card>
          <v-card-title
            >New Image<v-btn absolute top right @click="saveImage()"
              >SAVE</v-btn
            ></v-card-title
          >
          <v-card-text>
            <v-menu
              v-model="dateMenu"
              :close-on-content-click="false"
              :nudge-right="40"
              transition="scale-transition"
              offset-y
              min-width="290px"
            >
              <template #activator="{ on, attrs }">
                <v-text-field
                  v-model="date"
                  label="date"
                  prepend-icon="mdi-calendar"
                  readonly
                  v-bind="attrs"
                  v-on="on"
                ></v-text-field>
              </template>
              <v-date-picker
                v-model="date"
                @input="dateMenu = false"
              ></v-date-picker>
            </v-menu>
            <v-text-field v-model="name" label="insert name"></v-text-field>
            <v-text-field
              v-model="notify"
              label="insert notify interval"
            ></v-text-field>
            <v-file-input
              v-model="imageFile"
              label="select image"
            ></v-file-input>
          </v-card-text>
        </v-card>
      </v-form>
    </v-dialog>
    <v-dialog v-model="editDialog" max-width="480px">
      <v-form>
        <v-card>
          <v-card-title
            >Update Image<v-btn absolute top right @click="updateImage()"
              >SAVE</v-btn
            ></v-card-title
          >
          <v-card-text>
            <v-menu
              v-model="dateMenu"
              :close-on-content-click="false"
              :nudge-right="40"
              transition="scale-transition"
              offset-y
              min-width="290px"
            >
              <template #activator="{ on, attrs }">
                <v-text-field
                  v-model="date"
                  label="date"
                  prepend-icon="mdi-calendar"
                  readonly
                  v-bind="attrs"
                  v-on="on"
                ></v-text-field>
              </template>
              <v-date-picker
                v-model="date"
                @input="dateMenu = false"
              ></v-date-picker>
            </v-menu>
            <v-text-field v-model="name" label="insert name"></v-text-field>
            <v-text-field
              v-model="notify"
              label="insert notify interval"
            ></v-text-field>
            <v-file-input
              v-model="imageFile"
              label="select image"
            ></v-file-input>
          </v-card-text>
        </v-card>
      </v-form>
    </v-dialog>
    <v-dialog v-model="deleteConfirm" max-width="480px">
      <v-card>
        <v-card-title
          >Do you want to delete it?<v-btn
            absolute
            top
            right
            @click="deleteImage()"
            >DELETE</v-btn
          ></v-card-title
        >
      </v-card>
    </v-dialog>
    <v-col cols="12">
      <v-btn color="pink" fab class="ma-2" @click="showNewDialog()"
        ><v-icon>mdi-plus</v-icon></v-btn
      >
    </v-col>
    <template v-for="image in images">
      <v-col cols="12" xs="12" sm="12" md="6" lg="4" xl="4">
        <v-card class="pa-2 ma-2" outlined tile>
          <v-img
            :src="image.url"
            max-height="360"
            @click="showEditDialog(image)"
          >
          </v-img>
          <v-card-title
            @click="showEditDialog(image)"
            v-text="image.date"
          ></v-card-title>
          <v-card-text
            @click="showEditDialog(image)"
            v-text="image.name"
          ></v-card-text>
          <v-card-text @click="showEditDialog(image)"
            >notify interval: {{ image.notify }} days
          </v-card-text>
          <v-card-actions>
            <v-btn
              color="grey"
              absolute
              bottom
              right
              fab
              class="mb-12"
              @click="showDeleteConfirm(image.id)"
              ><v-icon>mdi-minus</v-icon></v-btn
            >
          </v-card-actions>
        </v-card>
      </v-col>
    </template>
  </v-row>
</template>

<script>
import { format } from 'date-fns'

export default {
  async asyncData({ app }) {
    try {
      const response = await app.$axios.$get('/api/list')
      return {
        images: response,
      }
    } catch (e) {
      console.log(e)
    }
  },
  data() {
    return {
      images: [],
      newDialog: false,
      editDialog: false,
      deleteConfirm: false,

      id: '',
      date: format(new Date(), 'yyyy-MM-dd'),
      name: '',
      notify: 0,
      imageFile: null,

      dateMenu: false,
    }
  },
  methods: {
    showNewDialog() {
      this.newDialog = true
    },

    showEditDialog(image) {
      this.id = image.id
      this.date = image.date
      this.name = image.name
      this.notify = image.notify
      this.editDialog = true
    },
    async saveImage() {
      const formData = new FormData()
      formData.append('date', this.date)
      formData.append('name', this.name)
      formData.append('notify', this.notify)
      formData.append('imageFile', this.imageFile)
      try {
        await this.$axios.post('/api/addImage', formData)
      } catch (e) {
        console.log(e)
      }
      location.reload()
    },
    async updateImage() {
      const formData = new FormData()
      formData.append('id', this.id)
      formData.append('date', this.date)
      formData.append('name', this.name)
      formData.append('notify', this.notify)
      formData.append('imageFile', this.imageFile)
      try {
        await this.$axios.put('/api/updateImage', formData)
      } catch (e) {
        console.log(e)
      }
      this.editDialog = false
      location.reload()
    },
    showDeleteConfirm(id) {
      this.id = id
      this.deleteConfirm = true
    },
    async deleteImage() {
      const formData = new FormData()
      formData.append('id', this.id)
      try {
        await this.$axios.put('/api/deleteImage', formData)
      } catch (e) {
        console.log(e)
      }
      this.deleteConfirm = false
      location.reload()
    },
  },
}
</script>
